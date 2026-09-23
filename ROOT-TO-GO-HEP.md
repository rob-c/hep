From ROOT to go-hep
===================

A translation table, for people who know what they want to do in ROOT and
want to know how it is spelled here. Everything below is pure Go: no ROOT
installation, no C++, no bindings.

The interactive prompt is `hep-shell`, which is to Go what `root` is to C++.
Everything in this page can be typed at it directly, and the packages below
are already imported when it starts. `hep-kernel` is the same session behind
a Jupyter notebook.

```
go install go-hep.org/x/hep/cmd/hep-shell@latest
go install go-hep.org/x/hep/cmd/hep-kernel@latest && hep-kernel -install
```

Files
-----

| ROOT | go-hep |
|---|---|
| `TFile *f = TFile::Open("f.root")` | `f, err := groot.Open("f.root")` |
| `TFile *f = TFile::Open("f.root","RECREATE")` | `f, err := groot.Create("f.root")` |
| `f->Close()` | `f.Close()` |
| `f->Get("name")` | `obj, err := f.Get("name")` |
| `f->WriteObject(h, "h")` | `f.Put("h", h)` |
| `f->ls()` | `root-ls f.root` |
| `TFile::ls()` on the command line | `root-dump`, `root-print`, `root-diff`, `root-merge`, `root-split` |

Trees
-----

| ROOT | go-hep |
|---|---|
| `TTree *t = (TTree*)f->Get("tree")` | `t := obj.(rtree.Tree)` |
| `t->GetEntries()` | `t.Entries()` |
| `t->Draw("pt")` | `rdraw.H1D(t, "pt")` |
| `t->Draw("pt", "pt>20")` | `rdraw.H1D(t, "pt", rdraw.Cut("pt>20"))` |
| `t->Draw("pt>>h(100,0,200)")` | `rdraw.H1D(t, "pt", rdraw.Bins(100, 0, 200))` |
| `t->Draw("y:x")` | `rdraw.H2D(t, "y:x")` |
| `t->Draw("z:y:x")` | `rdraw.H3D(t, "z:y:x")` |
| `t->Draw("y:x>>prof","","prof")` | `rdraw.P1D(t, "y:x")` (a TProfile) |
| `t->Draw("jet_pt")` (an array branch) | `rdraw.H1D(t, "jet_pt")` |
| `t->Draw("jet_pt[0]")` | `rdraw.H1D(t, "jet_pt[0]")` |
| `t->Draw("jet_pt", "jet_pt>30")` | `rdraw.H1D(t, "jet_pt", rdraw.Cut("jet_pt>30"))` |
| `t->Draw("Sum$(jet_pt)")` | `rdraw.H1D(t, "Sum$(jet_pt)")` |
| `t->Draw("Length$(jet_pt)")` | `rdraw.H1D(t, "Length$(jet_pt)")` |

A branch holding an array or a vector is looped over, as ROOT does it: one
fill per element rather than one per entry, with the cut and the weight
applied element by element too. ROOT's `Length$`, `Sum$`, `Min$`, `Max$`,
`MinIf$`, `MaxIf$`, `Alt$`, `Entry$`, `Entries$` and `Iteration$` all work and
mean what they mean in ROOT, so `Sum$(jet_pt) > 200` cuts on the entry while
`jet_pt > 30` cuts on the jets.

Collections an expression loops over must line up. `jet_pt:jet_eta` is fine;
`jet_pt:muon_pt` is reported rather than paired off in an order nobody asked
for, which is the one place this is deliberately stricter than ROOT.
| `t->Scan()` | `root-dump f.root` |
| `TChain` | `rtree.Chain(t1, t2, ...)` |
| `chain->Add("*.root")` | `rtree.ChainOf("tree", files...)` |

As in ROOT, `"y:x"` is y against x — the first expression is the vertical
axis. Expressions take the maths library under either name, so
`TMath::Abs(eta) < 2.5` and `abs(eta) < 2.5` both work.

Reading a tree entry by entry, where ROOT would use `SetBranchAddress`:

```go
var evt struct {
	Pt  float64
	Eta float64
}
r, err := rtree.NewReader(t, rtree.ReadVarsFromStruct(&evt))
defer r.Close()

err = r.Read(func(ctx rtree.RCtx) error {
	// evt is filled for this entry
	return nil
})
```

RDataFrame
----------

| ROOT | go-hep |
|---|---|
| `ROOT::RDataFrame df(*t)` | `df := rdf.New(t)` |
| `df.Define("z", "x+y")` | `df.Define("z", "x+y")` |
| `df.Filter("pt>20", "cut")` | `df.Filter("pt>20", "cut")` |
| `df.Count()` | `df.Count()` |
| `df.Histo1D({"h","",100,0,200}, "pt")` | `df.Histo1D("pt", rdf.Bins(100,0,200))` |
| `df.Mean("pt")`, `df.Sum("pt")` | `df.Mean("pt")`, `df.Sum("pt")` |
| `df.Report()->Print()` | `df.Report()` |
| `*h` (triggers the loop) | `h.Value()` |

Both are lazy and both read the tree once however much is asked of it. In Go
the results are handles and `Value` gives you the value, running the frame if
it has not run yet.

```go
df := rdf.New(t).
	Define("mt", "sqrt(2*pt*met*(1-cos(dphi)))").
	Filter("pt > 20 && abs(eta) < 2.5", "signal")

n := df.Count()
h := df.Histo1D("mt", rdf.Bins(100, 0, 200))

fmt.Println(n.Value(), h.Value().XMean())
```

Histograms
----------

| ROOT | go-hep |
|---|---|
| `new TH1D("h","",100,0,10)` | `hbook.NewH1D(100, 0, 10)` |
| `new TH2D(...)`, `new TH3D(...)` | `hbook.NewH2D(...)`, `hbook.NewH3D(...)` |
| `h->Fill(x)` | `h.Fill(x, 1)` |
| `h->Fill(x, w)` | `h.Fill(x, w)` |
| `h->GetEntries()` | `h.Entries()` |
| `h->GetMean()`, `h->GetRMS()` | `h.XMean()`, `h.XStdDev()` |
| `h->Integral()` | `h.Integral()` |
| `h->Add(h2)` | `hbook.AddH1D(h, h2)` |
| `h1->Divide(h2)` | `hbook.DivideH1D(h1, h2)` |
| `h3->Project3D("xy")` | `h3.ProjectionXY()` |
| `h3->ProjectionZ()` | `h3.ProjectionZ()` |

A profile -- ROOT's TProfile, the mean of y in bins of x -- is `hbook.P1D`,
filled by hand or straight from a tree with `rdraw.P1D` or `rdf.Profile1D`.
Each bin carries `YMean`, `YStdDev` and `YStdErr`: ROOT draws the error on
the mean by default and the spread when told to, and both are there.

`hbook` is the histogram; `rhist` is its ROOT file form. To write one:

```go
f.Put("h", rhist.NewH1DFrom(h))     // hbook -> TH1D
h := obj.(*rhist.H1D).AsH1D()       // TH1D -> hbook
```

The ROOT types in `rhist` can also be filled directly, if you would rather
stay with the names you know. All fifteen of them -- `TH1C/S/I/F/D` and the
same for 2 and 3 dimensions -- take the ROOT calls:

```go
h := rhist.NewH1D("h", "a title", 100, 0, 10)
h.Fill(x, w)
h.Scale(1 / h.Integral())
fmt.Println(h.Mean(), h.StdDev(), h.MaximumBin())
f.Put("h", h)
```

| ROOT | go-hep |
|---|---|
| `new TH1D("h","t",n,lo,hi)` | `rhist.NewH1D("h","t",n,lo,hi)` |
| `new TH1D("h","t",n,edges)` | `rhist.NewH1DFromEdges("h","t",edges)` |
| `h->FindBin(x)` | `h.FindBin(x)` |
| `h->GetBinContent(i)`, `SetBinContent` | `h.BinContent(i)`, `h.SetBinContent(i, v)` |
| `h->GetBinError(i)`, `SetBinError` | `h.BinError(i)`, `h.SetBinError(i, e)` |
| `h->Scale(f)`, `h->Reset()` | `h.Scale(f)`, `h.Reset()` |
| `h->GetMaximum()`, `GetMaximumBin()` | `h.Maximum()`, `h.MaximumBin()` |
| `h2->ProjectionX()` | `h2.ProjectionX("px")` |

Bins are numbered ROOT's way: 0 is the underflow, 1 to N the bins proper, and
N+1 the overflow. Note that `StdDev` is ROOT's `GetStdDev`, the spread of the
distribution as filled; `hbook`'s `XStdDev` carries Bessel's correction over
the effective number of entries and so reads slightly larger.

RNTuple
-------

RNTuple is the columnar format ROOT 7 introduced to replace TTree. `groot/exp/rntup`
reads it: open one by name, bind its fields to Go values, walk the entries.

```go
r, err := rntup.Open("data.root", "ntuple")
defer r.Close()

var (
        n  int32
        xs []float32
)
rvars := []rntup.ReadVar{
        {Name: "n", Value: &n},
        {Name: "xs", Value: &xs},
}

err = r.Read(rvars, func(entry uint64) error {
        fmt.Println(entry, n, xs)
        return nil
})
```

| ROOT | go-hep |
|---|---|
| `RNTupleReader::Open(name, file)` | `rntup.Open(file, name)` |
| `reader->GetNEntries()` | `r.Entries()` |
| `reader->GetView<T>("x")` | a `rntup.ReadVar` bound to a `*T` |
| `reader->LoadEntry(i)` | `r.ReadRange(rvars, i, i+1, fn)` |
| `reader->GetDescriptor()` | `r.Schema()` |

`rntup.NewReadVars(r)` builds the list from the schema when you do not know it
ahead of time, allocating the Go type each field calls for. The C++ types land
where you would expect: `std::string` on `string`, `std::vector<T>` on `[]T`,
`std::array<T,N>` on `[N]T`, a struct or a class and its bases on a struct,
and `std::variant` on an `any`. You may bind a struct of your own instead, so
long as its members line up.

Writing one works the same way round:

```go
var (
        n  int32
        xs []float64
)
w, err := rntup.Create("out.root", "ntuple", []rntup.WriteVar{
        {Name: "n", Value: &n},
        {Name: "xs", Value: &xs},
})
defer w.Close()

for i := range 1000 {
        n, xs = int32(i), []float64{float64(i)}
        err = w.Write()
}
```

The schema comes from the Go types, and the C++ type each one stands for is
recorded so another reader knows what it is looking at. Pages are compressed
one at a time.

Files written this way are read by [uproot], which is an implementation of the
format that owes nothing to this one; a test checks that, and skips where
uproot is not installed. What goes out is version 1.0.0.0 of the format with
the split column encodings, as ROOT writes: splitting puts the bytes of a
page's elements beside the bytes that resemble them, which costs nothing and
roughly halves what is left after compression.

[uproot]: https://github.com/scikit-hep/uproot5

Fitting
-------

| ROOT | go-hep |
|---|---|
| `h->Fit("gaus")` | `minuit.FitH1D(h, minuit.Gaussian, minuit.GausPars(h))` |
| `h->Fit("expo")` | `minuit.FitH1D(h, minuit.Exponential, pars)` |
| `h->Fit("pol2")` | `minuit.FitH1D(h, minuit.Polynomial(2), pars)` |
| `new TMinuit(n)` | `minuit.New(n)` |
| `gMinuit->SetFCN(fcn)` | `m.SetFCN(fcn)` |
| `gMinuit->mnparm(...)` | `m.Parameter(...)` |
| `gMinuit->mnexcm("MIGRAD",...)` | `m.Command("MIGRAD", ...)` |
| `gMinuit->mnpout(...)` | `m.Value(i)` |
| `gMinuit->mnerrs(...)` | `m.Errors(i)` |
| `gMinuit->mnstat(...)` | `m.Stats()` |
| `"SET ERR"`, `MIGRAD`, `HESSE`, `MINOS`, `SIMPLEX` | the same, through `m.Command` |

The minimiser is a fresh implementation of MIGRAD, HESSE, MINOS and SIMPLEX
from the published descriptions of them. It is not a translation of MINUIT's
Fortran nor of ROOT's C++, both of which are GPL and could not be carried in
a BSD tree.

RooFit
------

| RooFit | go-hep |
|---|---|
| `RooGaussian`, `RooExponential` | `pdf.Gaussian()`, `pdf.Exponential()` |
| `RooPolynomial`, `RooChebychev` | `pdf.Polynomial(n)`, `pdf.Chebychev(n, lo, hi)` |
| `RooCBShape`, `RooBifurGauss` | `pdf.CrystalBall()`, `pdf.BifurGauss()` |
| `RooBreitWigner`, `RooVoigtian` | `pdf.BreitWigner()`, `pdf.Voigtian()` |
| `RooArgusBG`, `RooLandau`, `RooPoisson` | `pdf.Argus()`, `pdf.Landau()`, `pdf.Poisson()` |
| `RooHistPdf`, `RooGenericPdf` | `pdf.Hist(h)`, `pdf.Formula(expr, pars)` |
| `RooBernstein`, `RooKeysPdf` | `pdf.Bernstein(n, lo, hi)`, `pdf.Keys(data, lo, hi, scale)` |
| `RooAddPdf` | `pdf.Add(pdfs, names)` |
| `RooProdPdf` (same observable) | `pdf.Mul(pdfs...)` |
| `RooProdPdf` (factorised) | `pdf.Factorise(pdfs...)` |
| `RooSimultaneous` | `pdf.FitSimultaneous(channels, pars)` |
| `pdf.fitTo(data)` | `pdf.FitUnbinned(data, p, lo, hi, pars)` |
| `pdf.fitTo(hist)` | `pdf.FitBinned(h, p, lo, hi, pars)` |
| `pdf.generate(x, n)` | `pdf.Generate(rnd, p, lo, hi, par, n)` |
| `RooMCStudy` | `pdf.Study{...}.Run(rnd, n)` |
| `SPlot` | `pdf.SPlot(data, sum, lo, hi, par)` |
| `fitTo(data, SumW2Error(true))` | `pdf.FitWeighted(data, weights, ...)` |
| `RooGaussian` constraint term | `pdf.Constrain(nll, i, mean, sigma)` |
| `RooFFTConvPdf` | `pdf.Convolve(f, g, lo, hi, n)` |
| `PiecewiseInterpolation`, `FlexibleInterpVar` | `pdf.NewMorph(nom, ups, downs, code)` |
| HistFactory model | `pdf.NewBinned(npar, samples, cons)` |
| `createProfile(par)` | `res.Scan(i, lo, hi, n)` |
| `AsymptoticCalculator`, `HypoTestInverter` | `pdf.UpperLimit(res, par, lo, hi, n, cl, asimov)` |
| discovery significance | `pdf.Discovery(res, par, nllNull)` |
| `RooFitResult::correlationMatrix()` | `res.Minuit.Correlation()` |
| `minos()` | `res.Minuit.Command("MINOS")` |

Coefficients multiply components that have been normalised over the fit
range, as RooFit's do, so a yield counts events. `SetErrorDef(0.5)` is
applied for you: an uncertainty is where the likelihood rises by half a unit.

```go
model := pdf.Add(
	[]pdf.PDF{pdf.Gaussian(), pdf.Exponential()},
	[]string{"nsig", "nbkg"},
)
res, err := pdf.FitUnbinned(data, model, 0, 10, pars)

nsig, nsigErr := res.Value(0)
scan, _ := res.Scan(0, nsig-4*nsigErr, nsig+4*nsigErr, 41)
lo, hi, ok := pdf.Interval(scan, res.Minuit.ErrorDef())
```

Functions
---------

| ROOT | go-hep |
|---|---|
| `new TF1("f","[0]*x+[1]",0,10)` | `rhist.NewF1("f", "[0]*x+[1]", 0, 10)` |
| `f->SetParameters(a, b)` | `f.SetParams([]float64{a, b})` |
| `f->Eval(x)` | `f.Eval(x)` |
| `f->GetParameter(i)` | `f.Params()[i]` |
| `new TF2`, `new TF3` | `rhist.NewF2`, `rhist.NewF3` |

The formula language is ROOT's: `x`, `y`, `z`, `t`, parameters as `[0]` or
`[name]`, the maths library under either spelling, and the `gaus`, `expo`,
`polN` and `landau` shorthands. The chebyshevs are refused by name, needing a
range a formula string does not carry.

Densities for fitting live in `fit/pdf`: gaussian, exponential, uniform,
polynomial, Chebychev, Crystal Ball, Breit-Wigner, bifurcated gaussian,
ARGUS, Landau, Voigtian, Poisson, a template from a histogram, and one
written as a formula.

Plotting
--------

| ROOT | go-hep |
|---|---|
| `new TCanvas` | `hplot.New()` |
| `h->Draw()` | `p.Add(hplot.NewH1D(h))` |
| `h2->Draw("colz")` | `p.Add(hplot.NewH2D(h2, nil))` |
| `h3->Draw()` | `p.Add(hplot.NewH3D(h3, hplot.H3DXY, nil))` |
| `f->Draw("same")` | `p.Add(hplot.NewFunction(fct))` |
| `c->Divide(2,2)` | `hplot.NewTiledPlot(...)` |
| `THStack` | `hplot.NewHStack(...)` |
| ratio plots by hand | `hplot.NewRatioPlot(...)` |
| `c->SaveAs("c.png")` | `p.Save(w, h, "c.png")` |

`Save` takes the extension from the filename: `.png`, `.pdf`, `.svg`, `.eps`,
`.jpg`, `.tex`.

A whole session
---------------

The ROOT macro:

```cpp
TFile *f = TFile::Open("data.root");
TTree *t = (TTree*)f->Get("tree");
TH1D *h = new TH1D("h", "", 60, -3, 7);
t->Draw("x >> h");
h->Fit("gaus");
c->SaveAs("fit.png");
```

and the same thing here, typed at `hep-shell` or compiled:

```go
f, err := groot.Open("data.root")
obj, err := f.Get("tree")
t := obj.(rtree.Tree)

h, err := rdraw.H1D(t, "x", rdraw.Bins(60, -3, 7))

fit, err := minuit.FitH1D(h, minuit.Gaussian, minuit.GausPars(h))

p := hplot.New()
p.Add(hplot.NewH1D(h))
p.Add(hplot.NewFunction(fit.Func(minuit.Gaussian)))
err = p.Save(16*vg.Centimeter, 12*vg.Centimeter, "fit.png")
```

Things ROOT does that this does not
-----------------------------------

Stated plainly, so nobody finds out the hard way:

- **ROOT-streamed objects inside an RNTuple.** A field holding a user class
  written through the ROOT streamer, rather than split into columns, is
  refused rather than guessed at.
- **Pairing up collections of different lengths.** ROOT will loop over two
  unrelated arrays together; `rdraw` and `rdf` report the mismatch instead,
  since it is rarely what was meant. Where it is, `Alt$` pads the shorter one.
- **Arbitrary C++ classes.** Types groot knows are read; a user class needs
  its streamer info to be turned into a Go type.
- **chebyshev** formula shapes, which need a range the string does not carry.
- **A C++ interpreter.** `hep-shell` and `hep-kernel` interpret Go. A ROOT
  macro is *translated* to Go rather than interpreted as C++ -- see below --
  and this page is the dictionary for what the translator does not cover.
- **TMVA and the GUI.** No equivalent. RooFit is largely covered -- see above -- but there is no workspace persistence here, so a model is built in code rather than loaded from a file.

From Python
-----------

`python/gohep` reads a ROOT tree into pyarrow or pandas, by running
`root2arrow` and handing the Arrow stream over. No ROOT, and nothing
reimplemented in Python:

```python
import gohep
df = gohep.read_dataframe("data.root", "tree")
```

Running an existing macro
-------------------------

A CINT macro can be translated into Go and run, without ROOT and without a
C++ interpreter anywhere:

```
$> hep-cint hsimple.C            # write the Go out
$> hep-cint -run hsimple.C       # translate, build and run it
$> hep-cint -o hsimple.go hsimple.C
```

At the prompt, `.x` takes a `.C` file the way ROOT's does:

```
hep [0] .x hsimple.C
```

and from a program, `cint.ROOT` is what a macro calls `gROOT`:

```go
err := cint.ROOT.Macro("hsimple.C")          // gROOT->Macro("hsimple.C")
err = cint.ROOT.ProcessLine(`.x hsimple.C`)  // gROOT->ProcessLine(...)
```

Nothing is interpreted. The macro becomes Go, the Go compiler builds it, and
the program runs — so a macro that runs at all is one that type-checks
throughout, and its mistakes are found by a compiler rather than on the line
that finally reached them. The trade is that a run costs a compile.

What comes out is meant to be kept. `hep-cint -o` writes Go that you can read,
edit and put under test, which is the real reason to translate a macro: it
stops being a macro.

| CINT | Go |
|---|---|
| `TH1F *h = new TH1F("h","t",100,0,10)` | `h := rhist.NewH1F("h","t",100,0,10)` |
| `h->Fill(x)` | `h.Fill(x, 1)` |
| `h->GetEntries()` | `h.Entries()` |
| `TFile *f = TFile::Open("d.root")` | `f := rt.OpenFile("d.root", "")` |
| `(TTree*)f->Get("t")` | `rt.Get(f, "t").(rtree.Tree)` |
| `TMath::Sqrt(x)` | `math.Sqrt(x)` |
| `gRandom->Gaus(0,1)` | `rt.GRandom.Gaus(0, 1)` |
| `std::vector<double> v; v.push_back(x)` | `var v []float64; v = append(v, x)` |
| `cout << x << endl` | `fmt.Println(x)` |
| `c->SaveAs("h.png")` | `c.SaveAs("h.png")` |

C++ widens a whole number to a real wherever one is wanted and Go does not, so
the conversions C++ left unwritten get written: `h->Fill(i)` becomes
`h.Fill(float64(i), 1)`.

The session part of ROOT — `gROOT`, `gDirectory`, graphics that draw as the
macro runs — has no equivalent in a compiled program. The part a macro
actually leans on (a current canvas, a current file, `gRandom`) is in
`cint/rt`; the rest is refused rather than faked, and so is any ROOT class or
method the translator does not know. It will name the line and say why. There
is no silent best effort: a translation that quietly did something else would
be worse than none.

Things this does that ROOT does not
-----------------------------------

- One static binary. No installation, no `thisroot.sh`, no dictionaries.
- Reads and writes HepMC, LHEF, SLHA, LCIO, FITS, NumPy, Arrow, YODA, CSV
  and RIO alongside ROOT files.
- Concurrency that is part of the language rather than a flag.
- `root-diff`, `root-merge`, `root-split`, `root-cp` as plain command-line
  tools that need no ROOT.
