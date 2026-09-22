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

`hbook` is the histogram; `rhist` is its ROOT file form. To write one:

```go
f.Put("h", rhist.NewH1DFrom(h))     // hbook -> TH1D
h := obj.(*rhist.H1D).AsH1D()       // TH1D -> hbook
```

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

- **RNTuple.** Only the anchor object is understood. A file whose data is in
  RNTuple rather than TTree cannot be read yet.
- **Arrays in Draw expressions.** ROOT loops over an array branch implicitly;
  `rdraw` and `rdf` refuse one instead of guessing which loop was meant. Read
  it with `rtree.Reader` and fill by hand.
- **Arbitrary C++ classes.** Types groot knows are read; a user class needs
  its streamer info to be turned into a Go type.
- **chebyshev** formula shapes, which need a range the string does not carry.
- **A C++ interpreter.** `hep-shell` and `hep-kernel` interpret Go. A ROOT
  macro has to be translated, and this page is the dictionary.
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

Things this does that ROOT does not
-----------------------------------

- One static binary. No installation, no `thisroot.sh`, no dictionaries.
- Reads and writes HepMC, LHEF, SLHA, LCIO, FITS, NumPy, Arrow, YODA, CSV
  and RIO alongside ROOT files.
- Concurrency that is part of the language rather than a flag.
- `root-diff`, `root-merge`, `root-split`, `root-cp` as plain command-line
  tools that need no ROOT.
