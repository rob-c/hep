// Copyright ©2018 The go-hep Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xrdfs // import "go-hep.org/x/hep/xrootd/xrdfs"

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go-hep.org/x/hep/xrootd/internal/xrdenc"
)

// StatFlags identifies the entry's attributes.
type StatFlags int32

const (
	// StatIsFile indicates that entry is a regular file if no other flag is specified.
	StatIsFile StatFlags = 0
	// StatIsExecutable indicates that entry is either an executable file or a searchable directory.
	StatIsExecutable StatFlags = 1
	// StatIsDir indicates that entry is a directory.
	StatIsDir StatFlags = 2
	// StatIsOther indicates that entry is neither a file nor a directory.
	StatIsOther StatFlags = 4
	// StatIsOffline indicates that the file is not online (i. e., on disk).
	StatIsOffline StatFlags = 8
	// StatIsReadable indicates that read access to that entry is allowed.
	StatIsReadable StatFlags = 16
	// StatIsWritable indicates that write access to that entry is allowed.
	StatIsWritable StatFlags = 32
	// StatIsPOSCPending indicates that the file was created with kXR_posc and has not yet been successfully closed.
	// kXR_posc is an option of open request indicating that the "Persist On Successful Close" processing is enabled and
	// the file will be persisted only when it has been explicitly closed.
	StatIsPOSCPending StatFlags = 64
)

// EntryStat holds the entry name and the entry stat information.
//
// The first six fields are the stat line every server sends. The five that
// follow are the extended tail that a server may append — EOS does, and so does
// any server answering a kXR_dcksm listing — and they are meaningful only when
// HasExtendedInfo is set. Checksum is carried on the same line but is not part
// of that tail: it is the answer to kXR_dcksm alone.
type EntryStat struct {
	EntryName   string    // EntryName is the name of entry.
	HasStatInfo bool      // HasStatInfo indicates if the following stat information is valid.
	ID          int64     // ID is the OS-dependent identifier assigned to this entry.
	EntrySize   int64     // EntrySize is the decimal size of the entry.
	Flags       StatFlags // Flags identifies the entry's attributes.
	Mtime       int64     // Mtime is the last modification time in Unix time units.

	HasExtendedInfo bool   // HasExtendedInfo indicates if the following extended stat information is valid.
	Ctime           int64  // Ctime is the last status change time in Unix time units.
	Atime           int64  // Atime is the last access time in Unix time units.
	Perm            uint32 // Perm holds the permission bits, as the "0644" token on the wire.
	Owner           string // Owner is the owning user, as a name or a decimal uid depending on the server.
	Group           string // Group is the owning group, as a name or a decimal gid depending on the server.

	// Checksum is the "algorithm:hexdigest" token a server appends when the
	// dirlist request asked for kXR_dcksm. The digest reads "none" for an entry
	// that has none: a directory, a symbolic link, or a file the server could
	// not read. It is empty when no checksum was asked for.
	Checksum string
}

// ChecksumAlgo returns the algorithm named by [EntryStat.Checksum], and
// ChecksumValue the digest it produced. Both are empty when no checksum was
// asked for; the value alone is empty when the server had none to give, which
// is how "the file is there but its digest is not" is told apart from "this
// listing carried no checksums at all".
func (es EntryStat) ChecksumAlgo() string {
	algo, _, ok := strings.Cut(es.Checksum, ":")
	if !ok {
		return ""
	}
	return algo
}

// ChecksumValue returns the hexadecimal digest of [EntryStat.Checksum].
// See [EntryStat.ChecksumAlgo].
func (es EntryStat) ChecksumValue() string {
	_, digest, ok := strings.Cut(es.Checksum, ":")
	if !ok || digest == noChecksum {
		return ""
	}
	return digest
}

// noChecksum is what a server puts where a digest would go for an entry that
// cannot have one.
const noChecksum = "none"

// EntryStatFrom creates an EntryStat that represents same information as the provided info.
func EntryStatFrom(info os.FileInfo) EntryStat {
	es := EntryStat{
		EntryName:   info.Name(),
		EntrySize:   info.Size(),
		Mtime:       info.ModTime().Unix(),
		HasStatInfo: true,
	}
	if info.IsDir() {
		es.Flags |= StatIsDir
	}
	if info.Mode()&0400 != 0 {
		es.Flags |= StatIsReadable
	}
	if info.Mode()&0200 != 0 {
		es.Flags |= StatIsWritable
	}
	return es
}

// EntryStatExtendedFrom creates an EntryStat that carries the extended tail as
// well: the two extra timestamps, the permission bits and the ownership.
//
// How much of that this port can see depends on the port. Everything os.FileInfo
// exposes is filled in everywhere; the fields that need the underlying stat
// buffer are zero where the operating system does not offer one through
// syscall.
func EntryStatExtendedFrom(info os.FileInfo) EntryStat {
	es := EntryStatFrom(info)
	es.HasExtendedInfo = true
	es.Perm = uint32(info.Mode().Perm())
	es.Ctime, es.Atime, es.Owner, es.Group = sysStat(info)
	return es
}

// Name implements os.FileInfo.
func (es EntryStat) Name() string {
	return es.EntryName
}

// Size implements os.FileInfo.
func (es EntryStat) Size() int64 {
	return es.EntrySize
}

// ModTime implements os.FileInfo.
func (es EntryStat) ModTime() time.Time {
	return time.Unix(es.Mtime, 0)
}

// Sys implements os.FileInfo.
func (es EntryStat) Sys() any {
	return nil
}

// Mode implements os.FileInfo.
func (es EntryStat) Mode() os.FileMode {
	var mode os.FileMode
	if es.IsDir() {
		mode |= os.ModeDir
	}
	if es.HasExtendedInfo {
		// The extended tail carries the real permission bits, so there is no
		// need to widen the three the flags word can tell apart into the nine
		// a caller expects.
		return mode | os.FileMode(es.Perm).Perm()
	}
	if es.IsWritable() {
		mode |= 0222
	}
	if es.IsReadable() {
		mode |= 0444
	}
	return mode
}

// IsExecutable indicates whether this entry is either an executable file or a searchable directory.
func (es EntryStat) IsExecutable() bool {
	return es.Flags&StatIsExecutable != 0
}

// IsDir indicates whether this entry is a directory.
func (es EntryStat) IsDir() bool {
	return es.Flags&StatIsDir != 0
}

// IsOther indicates whether this entry is neither a file nor a directory.
func (es EntryStat) IsOther() bool {
	return es.Flags&StatIsOther != 0
}

// IsOffline indicates whether this the file is not online (i. e., on disk).
func (es EntryStat) IsOffline() bool {
	return es.Flags&StatIsOffline != 0
}

// IsReadable indicates whether this read access to that entry is allowed.
func (es EntryStat) IsReadable() bool {
	return es.Flags&StatIsReadable != 0
}

// IsWritable indicates whether this write access to that entry is allowed.
func (es EntryStat) IsWritable() bool {
	return es.Flags&StatIsWritable != 0
}

// IsPOSCPending indicates whether this the file was created with kXR_posc and has not yet been successfully closed.
// kXR_posc is an option of open request indicating that the "Persist On Successful Close" processing is enabled and
// the file will be persisted only when it has been explicitly closed.
func (es EntryStat) IsPOSCPending() bool {
	return es.Flags&StatIsPOSCPending != 0
}

// MarshalXrd implements xrdproto.Marshaler.
func (o EntryStat) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	if !o.HasStatInfo {
		return nil
	}

	idStr := strconv.Itoa(int(o.ID))
	sizeStr := strconv.Itoa(int(o.EntrySize))
	flagsStr := strconv.Itoa(int(o.Flags))
	mtimeStr := strconv.Itoa(int(o.Mtime))

	line := idStr + " " + sizeStr + " " + flagsStr + " " + mtimeStr
	if o.HasExtendedInfo {
		// The permission bits go out as a four-digit octal token, which is what
		// the readers of this line scan back with strtoul(.., 8): written in
		// decimal, 0644 would come back as 0420.
		line += " " + strconv.FormatInt(o.Ctime, 10) +
			" " + strconv.FormatInt(o.Atime, 10) +
			" " + fmt.Sprintf("%04o", o.Perm&statPermMask) +
			" " + owned(o.Owner) + " " + owned(o.Group)
	}
	if o.Checksum != "" {
		// The checksum sits after the stat fields and inside brackets, so a
		// reader that does not know about kXR_dcksm still finds the fields it
		// came for at the offsets it expects.
		line += " [ " + o.Checksum + " ]"
	}

	wBuffer.WriteBytes([]byte(line))
	return nil
}

// statPermMask is the part of a mode this line carries: the twelve bits of
// permission and setuid/setgid/sticky, and not the file type above them, which
// travels in the flags word.
const statPermMask = 0o7777

// owned returns the token to put on the wire for an owner or a group. A server
// that does not know one still has to leave a field there, or every field after
// it is read as the wrong one.
func owned(name string) string {
	if name == "" {
		return "0"
	}
	return name
}

// UnmarshalXrd implements xrdproto.Unmarshaler.
func (o *EntryStat) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	var buf []byte
	for rBuffer.Len() != 0 {
		b := rBuffer.ReadU8()
		if b == '\x00' || b == '\n' {
			break
		}
		buf = append(buf, b)
	}

	buf, checksum := cutChecksum(buf)

	stats := bytes.Split(buf, []byte{' '})
	if len(stats) < 4 {
		return fmt.Errorf("xrootd: statinfo \"%s\" doesn't have enough fields, expected format is: \"id size flags modtime\"", buf)
	}

	id, err := strconv.Atoi(string(stats[0]))
	if err != nil {
		return err
	}
	size, err := strconv.Atoi(string(stats[1]))
	if err != nil {
		return err
	}
	flags, err := strconv.Atoi(string(stats[2]))
	if err != nil {
		return err
	}
	mtime, err := strconv.Atoi(string(stats[3]))
	if err != nil {
		return err
	}

	o.HasStatInfo = true
	o.ID = int64(id)
	o.EntrySize = int64(size)
	o.Mtime = int64(mtime)
	o.Flags = StatFlags(flags)
	o.Checksum = checksum

	// The extended tail is all five fields or none of them: a line that stops
	// partway through is one this reader does not understand, and guessing
	// which of the five arrived would put a ctime in the owner.
	o.HasExtendedInfo = false
	if len(stats) >= 9 {
		ctime, err := strconv.ParseInt(string(stats[4]), 10, 64)
		if err != nil {
			return err
		}
		atime, err := strconv.ParseInt(string(stats[5]), 10, 64)
		if err != nil {
			return err
		}
		perm, err := strconv.ParseUint(string(stats[6]), 8, 32)
		if err != nil {
			return err
		}
		o.HasExtendedInfo = true
		o.Ctime = ctime
		o.Atime = atime
		o.Perm = uint32(perm)
		o.Owner = string(stats[7])
		o.Group = string(stats[8])
	}

	return rBuffer.Err()
}

// cutChecksum takes the " [ algorithm:hexdigest ]" token off the end of a stat
// line, returning what is left of the line and the token's contents.
func cutChecksum(line []byte) (rest []byte, checksum string) {
	const (
		lead = " [ "
		tail = " ]"
	)

	if !bytes.HasSuffix(line, []byte(tail)) {
		return line, ""
	}
	i := bytes.LastIndex(line, []byte(lead))
	if i < 0 {
		return line, ""
	}
	return line[:i], string(line[i+len(lead) : len(line)-len(tail)])
}

// VirtualFSStat holds the virtual file system information.
type VirtualFSStat struct {
	NumberRW           int // NumberRW is the number of nodes that can provide read/write space.
	FreeRW             int // FreeRW is the size, in megabytes, of the largest contiguous area of read/write free space.
	UtilizationRW      int // UtilizationRW is the percent utilization of the partition represented by FreeRW.
	NumberStaging      int // NumberStaging is the number of nodes that can provide staging space.
	FreeStaging        int // FreeStaging is the size, in megabytes, of the largest contiguous area of staging free space.
	UtilizationStaging int // UtilizationStaging is the percent utilization of the partition represented by FreeStaging.
}

// MarshalXrd implements xrdproto.Marshaler
func (o VirtualFSStat) MarshalXrd(wBuffer *xrdenc.WBuffer) error {
	nrw := strconv.Itoa(o.NumberRW)
	frw := strconv.Itoa(o.FreeRW)
	urw := strconv.Itoa(o.UtilizationRW)
	nstg := strconv.Itoa(o.NumberStaging)
	fstg := strconv.Itoa(o.FreeStaging)
	ustg := strconv.Itoa(o.UtilizationStaging)
	wBuffer.WriteBytes([]byte(nrw + " " + frw + " " + urw + " " + nstg + " " + fstg + " " + ustg))
	return nil
}

// UnmarshalXrd implements xrdproto.Unmarshaler
func (o *VirtualFSStat) UnmarshalXrd(rBuffer *xrdenc.RBuffer) error {
	var buf []byte
	for rBuffer.Len() != 0 {
		b := rBuffer.ReadU8()
		if b == '\x00' || b == '\n' {
			break
		}
		buf = append(buf, b)
	}

	stats := bytes.Split(buf, []byte{' '})
	if len(stats) < 6 {
		return fmt.Errorf("xrootd: virtual statinfo \"%s\" doesn't have enough fields, expected format is: \"nrw frw urw nstg fstg ustg\"", buf)
	}

	nrw, err := strconv.Atoi(string(stats[0]))
	if err != nil {
		return err
	}
	frw, err := strconv.Atoi(string(stats[1]))
	if err != nil {
		return err
	}
	urw, err := strconv.Atoi(string(stats[2]))
	if err != nil {
		return err
	}
	nstg, err := strconv.Atoi(string(stats[3]))
	if err != nil {
		return err
	}
	fstg, err := strconv.Atoi(string(stats[4]))
	if err != nil {
		return err
	}
	ustg, err := strconv.Atoi(string(stats[5]))
	if err != nil {
		return err
	}

	o.NumberRW = nrw
	o.FreeRW = frw
	o.UtilizationRW = urw
	o.NumberStaging = nstg
	o.FreeStaging = fstg
	o.UtilizationStaging = ustg

	return rBuffer.Err()
}

var (
	_ os.FileInfo = (*EntryStat)(nil)
)
