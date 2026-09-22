go-hep from Python
==================

Reading ROOT files in Python without a ROOT installation, by way of go-hep.

```python
import gohep

tbl = gohep.read_tree("data.root", "tree")       # pyarrow.Table
df  = gohep.read_dataframe("data.root", "tree")  # pandas.DataFrame

for batch in gohep.read_batches("big.root", "tree"):
    ...                                          # pyarrow.RecordBatch
```

This module reimplements nothing. It runs go-hep's `root2arrow`, which reads
the ROOT file and writes Arrow, and hands the stream to pyarrow. Arrow is the
boundary between the two, so the data is not copied into Python objects on the
way across and is not parsed twice.

Install
-------

```
go install go-hep.org/x/hep/cmd/root2arrow@latest
pip install pyarrow          # and pandas, for read_dataframe
```

then put this directory on your `PYTHONPATH`. `GOHEP_ROOT2ARROW` points at the
binary if it is not on `PATH`.

Why
---

`uproot` already reads ROOT files in Python and does it well. What this offers
instead is go-hep's reader: one static binary, no ROOT, and the conversion
running outside the interpreter. Whether that is worth a subprocess depends on
what you are doing, and for a lot of work it will not be.
