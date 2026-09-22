# RNTuple reference files

These files were written by ROOT and are used to check that `groot/exp/rntup`
reads the RNTuple binary format the way ROOT writes it.

They come from [scikit-hep-testdata], which is BSD-3-Clause licensed, the same
as go-hep. The macros that generated them live in that repository under
`dev/make-root/`, and say exactly what each file holds; the tests assert
against those values.

The name of each file ends in the version of the binary format specification
it was written against, `v1-0-0-0` for the first release of RNTuple.

[scikit-hep-testdata]: https://github.com/scikit-hep/scikit-hep-testdata
