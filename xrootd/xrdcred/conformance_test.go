// Copyright ©2026 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Conformance for asking a user for a credential.
//
// Two properties matter more than anything the menu looks like. The first is
// that this never runs where there is no user: a prompt on a pipe is a hang, and
// a hang inside a batch job is worse than the authorization error it was trying
// to prevent. The second is that a credential goes from the user to the thing
// that needs it and no further — a pasted token is not echoed and never appears
// in an error, and a private-key passphrase is not read by this process at all
// but typed straight into voms-proxy-init with the terminal handed over.
//
// The tests below drive the prompter through a pipe, which is exactly what a
// terminal is not: it lets every question and answer be scripted, at the cost of
// the no-echo read, which needs a real tty and is pinned by inspection instead.

package xrdcred

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-hep.org/x/hep/xrootd"
	"go-hep.org/x/hep/xrootd/xrdproto/auth"
)

// scripted returns a Terminal reading the given answers, and the buffer holding
// everything it printed.
func scripted(t *testing.T, answers ...string) (*Terminal, *strings.Builder) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not make a pipe: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	go func() {
		defer w.Close()
		for _, a := range answers {
			if _, err := w.WriteString(a + "\n"); err != nil {
				return
			}
		}
	}()

	out := new(strings.Builder)
	return &Terminal{In: r, Out: out}, out
}

func TestConformance_APromptWithNoTerminalIsDeclinedNotAttempted(t *testing.T) {
	// This is what keeps the prompter safe to enable unconditionally in a
	// command: on a pipe it refuses at once, the client carries on down the
	// server's list, and a job that redirects its input behaves as it always did.
	term, out := scripted(t)

	_, err := term.PromptCredential(context.Background(), xrootd.CredentialRequest{Provider: "gsi"})
	if !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("the prompter answered on a pipe: %v", err)
	}
	if out.String() != "" {
		t.Fatalf("a question was asked with nobody to answer it:\n%s", out)
	}
}

func TestConformance_AProtocolThisClientCannotSupplyIsNotAQuestion(t *testing.T) {
	// There is no answer a user could give for a protocol this client does not
	// implement, and asking would make them go and find a credential that would
	// then be ignored.
	term := &Terminal{}
	if got := term.options("nosuchsec"); got != nil {
		t.Fatalf("a protocol nothing can supply offered %d answers", len(got))
	}
}

func TestConformance_EveryQuestionCanBeAnsweredWithNo(t *testing.T) {
	// A user who does not have the credential to hand must be able to get out of
	// the prompt, and getting out has to mean the transfer proceeds as it would
	// have — not that it aborts.
	term := &Terminal{}
	for _, provider := range []string{"gsi", "ztn", "sss"} {
		opts := term.options(provider)
		if len(opts) == 0 {
			t.Fatalf("%s offers no answers at all", provider)
		}
		last := opts[len(opts)-1]
		if last.key != "n" {
			t.Errorf("the last answer for %s is %q, want the way out", provider, last.key)
		}
		if _, err := last.run(context.Background(), term, xrootd.CredentialRequest{}); !errors.Is(err, ErrDeclined) {
			t.Errorf("declining %s reported %v, want a decline", provider, err)
		}
	}
}

func TestConformance_WithoutATheToolThereIsNothingToRun(t *testing.T) {
	// krb5 has exactly one answer — run kinit — so where kinit is not installed,
	// or running tools is forbidden, the only remaining choice is "no". That is
	// not a question worth interrupting a transfer for.
	term := &Terminal{NoExec: true}
	if got := term.options("krb5"); got != nil {
		t.Fatalf("krb5 asked a question with nothing to offer: %v", got)
	}
	if got := term.options("gsi"); len(got) != 2 {
		// gsi can still be answered with a path, so the question survives — one
		// fewer choice, not none.
		t.Fatalf("gsi offers %d answers with tools forbidden, want the file and the way out", len(got))
	}
}

func TestConformance_TheQuestionSaysWhatIsMissingAndWhereItLooked(t *testing.T) {
	// A user asked for an X.509 proxy generally knows they have one. What they do
	// not know — and what turns the question into a two-second answer — is that
	// the client looked somewhere else, or that what it found had expired.
	term, out := scripted(t)
	term.describe(xrootd.CredentialRequest{
		Provider: "gsi",
		Server:   "eoslhcb.cern.ch:1094",
		Missing: &auth.Missing{
			Provider: "gsi",
			What:     "X.509 proxy",
			Searched: []string{"/tmp/x509up_u1000"},
			Err:      errors.New("the proxy expired at 2026-07-30T09:00:00Z"),
		},
	})

	for _, want := range []string{"eoslhcb.cern.ch:1094", "X.509 proxy", "gsi", "/tmp/x509up_u1000", "expired"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the question does not mention %q:\n%s", want, out)
		}
	}
}

func TestConformance_AQuestionWithNothingKnownAboutItStillNamesTheProtocol(t *testing.T) {
	// A protocol the client does not implement has no Missing to describe, and
	// the question still has to say who is asking for what.
	term, out := scripted(t)
	term.describe(xrootd.CredentialRequest{Provider: "unix42", Server: "server:1094"})

	for _, want := range []string{"unix42", "server:1094"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the question does not mention %q:\n%s", want, out)
		}
	}
}

func TestConformance_AnEmptyAnswerTakesTheFirstChoice(t *testing.T) {
	// Pressing return is what a user does, and it has to mean the thing they
	// most likely want rather than an error.
	term, _ := scripted(t, "")
	opts := []option{{key: "1", text: "first"}, {key: "n", text: "no"}}

	got, err := term.choose(context.Background(), opts)
	if err != nil {
		t.Fatalf("could not read the answer: %v", err)
	}
	if got.key != "1" {
		t.Fatalf("an empty answer chose %q, want the first choice", got.key)
	}
}

func TestConformance_AnAnswerThatIsNotAChoiceIsAskedAgain(t *testing.T) {
	// A typo must not be read as a decline: the credential is still missing and
	// the user still meant to supply it.
	term, out := scripted(t, "7", "N")
	opts := []option{{key: "1", text: "first"}, {key: "n", text: "no"}}

	got, err := term.choose(context.Background(), opts)
	if err != nil {
		t.Fatalf("could not read the answer: %v", err)
	}
	if got.key != "n" {
		t.Fatalf("the second answer chose %q, want %q", got.key, "n")
	}
	if !strings.Contains(out.String(), `"7" is not one of the choices`) {
		t.Errorf("the user was not told why they were asked again:\n%s", out)
	}
}

func TestConformance_AQuestionNobodyAnsweredDoesNotOutliveTheCaller(t *testing.T) {
	// The prompt happens inside whatever operation needed the connection, and
	// that operation has a deadline. A user who walked away must not hold it.
	term, _ := scripted(t) // nothing will ever be typed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := term.ask(ctx, "path", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("the question outlived its caller: %v", err)
	}
}

func TestConformance_ATokenIsTakenFromTheFileTheUserNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.jwt")
	if err := os.WriteFile(path, []byte("the.jwt.token\n"), 0600); err != nil {
		t.Fatalf("could not write the token: %v", err)
	}

	term, out := scripted(t, path)
	a, err := loadTokenFile(context.Background(), term, xrootd.CredentialRequest{Provider: "ztn"})
	if err != nil {
		t.Fatalf("could not read the token file: %v", err)
	}
	if got := a.Provider(); got != "ztn" {
		t.Fatalf("the credential is for %q, want %q", got, "ztn")
	}

	req, err := a.Request(nil)
	if err != nil {
		t.Fatalf("the credential could not be used: %v", err)
	}
	if got, want := req.Credentials, "ztn\x00the.jwt.token"; got != want {
		t.Fatalf("the credential carries %q, want %q", got, want)
	}
	if strings.Contains(out.String(), "the.jwt.token") {
		// A bearer token is the whole credential, and a terminal keeps its
		// scrollback.
		t.Fatalf("the token was printed to the terminal:\n%s", out)
	}
}

func TestConformance_ATokenFileThatIsUnusableIsReportedNotAccepted(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.jwt")
	if err := os.WriteFile(empty, []byte("\n\n"), 0600); err != nil {
		t.Fatalf("could not write the file: %v", err)
	}

	for _, tc := range []struct {
		name   string
		answer string
	}{
		{"no such file", filepath.Join(dir, "absent.jwt")},
		{"a file holding no token", empty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term, _ := scripted(t, tc.answer)
			if _, err := loadTokenFile(context.Background(), term, xrootd.CredentialRequest{}); err == nil {
				t.Fatal("an unusable file was accepted as a token")
			}
		})
	}
}

func TestConformance_APathQuestionOffersNoAnswerThatIsKnownToBeWrong(t *testing.T) {
	// The conventional location is what was searched and did not work, so
	// offering it back as the default answers the question with the answer that
	// already failed. A blank answer means "I cannot", not "use that one".
	for _, tc := range []struct {
		name string
		run  func(context.Context, *Terminal, xrootd.CredentialRequest) (auth.Auther, error)
	}{
		{"a proxy", loadProxyFile},
		{"a token file", loadTokenFile},
		{"a keytab", loadKeytabFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term, out := scripted(t, "")
			_, err := tc.run(context.Background(), term, xrootd.CredentialRequest{})
			if !errors.Is(err, ErrDeclined) {
				t.Fatalf("a blank answer gave %v, want %v", err, ErrDeclined)
			}
			if got := out.String(); !strings.Contains(got, "blank to skip") {
				t.Fatalf("the question does not say a blank answer is allowed:\n%s", got)
			}
			if strings.Contains(out.String(), "[/") {
				t.Fatalf("the question offers a path that was already searched:\n%s", out)
			}
		})
	}
}

func TestConformance_APathTheUserTypesIsThePathTheyMeant(t *testing.T) {
	// People type "~/proxy" and "$HOME/proxy"; a client that opens those
	// literally reports that a file the user can see does not exist.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("this machine has no home directory: %v", err)
	}
	t.Setenv("XRDCRED_TEST_DIR", "/somewhere")

	for _, tc := range []struct{ in, want string }{
		{"~", home},
		{"~/x509up", filepath.Join(home, "x509up")},
		{"$XRDCRED_TEST_DIR/x509up", "/somewhere/x509up"},
		{"/tmp/x509up_u1000", "/tmp/x509up_u1000"},
		{"relative/path", "relative/path"},
		{"~notauser/x", "~notauser/x"}, // another user's home is not ours to guess
	} {
		if got := expand(tc.in); got != tc.want {
			t.Errorf("%q expands to %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestConformance_AKeytabTheUserNamesBecomesTheCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sss.keytab")
	const key = "000102030405060708090a0b0c0d0e0f"
	if err := os.WriteFile(path, []byte("0 N:9 k:"+key+" u:gopher g:hep n:go-hep e:0\n"), 0600); err != nil {
		t.Fatalf("could not write the keytab: %v", err)
	}

	term, out := scripted(t, path)
	a, err := loadKeytabFile(context.Background(), term, xrootd.CredentialRequest{Provider: "sss"})
	if err != nil {
		t.Fatalf("could not use the keytab: %v", err)
	}
	if got := a.Provider(); got != "sss" {
		t.Fatalf("the credential is for %q, want %q", got, "sss")
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("the user was not told which keytab was used:\n%s", out)
	}
}

func TestConformance_AKeytabThatCannotBeUsedIsNotACredential(t *testing.T) {
	term, _ := scripted(t, filepath.Join(t.TempDir(), "absent.keytab"))
	if _, err := loadKeytabFile(context.Background(), term, xrootd.CredentialRequest{}); err == nil {
		t.Fatal("a keytab that is not there produced a credential")
	}
}

func TestConformance_AProxyTheUserNamesIsCheckedBeforeItIsUsed(t *testing.T) {
	// Naming a file is not the same as having a usable proxy in it, and the
	// point of the prompt is to fail here — where the user is standing — rather
	// than at the server.
	term, _ := scripted(t, filepath.Join(t.TempDir(), "absent"))
	if _, err := loadProxyFile(context.Background(), term, xrootd.CredentialRequest{}); err == nil {
		t.Fatal("a proxy that is not there was accepted")
	}
}

func TestConformance_ATerminalIsWhatAUserCanAnswerOn(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not make a pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	if isTerminal(r) {
		t.Error("a pipe was taken for a terminal")
	}
	if isTerminal(nil) {
		// A nil input is what a daemon has; taking it for a terminal would
		// dereference it on the first read.
		t.Error("nothing at all was taken for a terminal")
	}
}

func TestConformance_ALineIsWhatTheUserTypedUpToTheReturn(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
		fail bool
	}{
		{name: "a line", in: "hello\n", want: "hello"},
		{name: "only the return", in: "\n", want: ""},
		{name: "a line with no return before the end", in: "hello", want: "hello"},
		{name: "nothing at all", in: "", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("could not make a pipe: %v", err)
			}
			defer r.Close()
			go func() {
				defer w.Close()
				_, _ = w.WriteString(tc.in)
			}()

			got, err := readLineFrom(r)
			switch {
			case tc.fail && err == nil:
				t.Fatalf("input that ended read as %q", got)
			case !tc.fail && err != nil:
				t.Fatalf("could not read the line: %v", err)
			case got != tc.want:
				t.Fatalf("the line is %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConformance_ALineIsReadOneByteAtATime(t *testing.T) {
	// Reading ahead past the newline would swallow the bytes a following no-echo
	// read needs, which is how a paste of "path\ntoken" loses the token.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("could not make a pipe: %v", err)
	}
	defer r.Close()
	go func() {
		defer w.Close()
		_, _ = w.WriteString("first\nsecond\n")
	}()

	if got, err := readLineFrom(r); err != nil || got != "first" {
		t.Fatalf("the first line is (%q, %v), want %q", got, err, "first")
	}
	if got, err := readLineFrom(r); err != nil || got != "second" {
		t.Fatalf("the second line is (%q, %v), want the bytes that were not consumed", got, err)
	}
}

func TestConformance_AToolThatIsNotInstalledIsNotOffered(t *testing.T) {
	if haveTool("definitely-not-a-real-command-xrdcred") {
		t.Error("a command that does not exist was reported as runnable")
	}
	if !haveTool("sh") {
		t.Error("sh was not found on a POSIX system")
	}
}

func TestConformance_TheVOIsGuessedFromWhatTheUserAlreadySet(t *testing.T) {
	t.Setenv("VOMS_VO", "lhcb")
	if got := guessVO(); got != "lhcb" {
		t.Fatalf("the VO was guessed as %q, want %q", got, "lhcb")
	}
}

func TestConformance_ThePrompterIsWhatTheClientAsks(t *testing.T) {
	var _ xrootd.CredentialPrompter = NewTerminal()
	var _ xrootd.CredentialPrompter = &Terminal{}
}

func TestConformance_AQuestionSaysWhatElseTheServerWouldAccept(t *testing.T) {
	// The client asks about each protocol it cannot supply, in the server's
	// order. A user holding an X.509 proxy, asked first for a Kerberos ticket,
	// has to be able to see that saying no leads to the question they can
	// answer — otherwise "no" looks like giving up.
	term, out := scripted(t)
	term.describe(xrootd.CredentialRequest{
		Provider: "krb5",
		Server:   "server:1094",
		Offered:  []string{"krb5", "gsi", "unix"},
	})

	got := out.String()
	if !strings.Contains(got, "gsi, unix") {
		t.Errorf("the question does not say what else would work:\n%s", got)
	}
	if strings.Contains(got, "accepts: krb5") {
		t.Errorf("the protocol being asked about is listed as an alternative to itself:\n%s", got)
	}
}

func TestConformance_TheOnlyProtocolOnOfferHasNoAlternatives(t *testing.T) {
	term, out := scripted(t)
	term.describe(xrootd.CredentialRequest{Provider: "gsi", Server: "server:1094", Offered: []string{"gsi"}})

	if strings.Contains(out.String(), "also accepts") {
		t.Errorf("alternatives were offered where there are none:\n%s", out)
	}
}
