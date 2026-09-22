# Copyright ©2026 The go-hep Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

"""Read ROOT files from Python, through go-hep.

The point of this module is not to reimplement anything in Python. It runs
go-hep's ``root2arrow``, which reads the ROOT file and writes Arrow, and hands
the result straight to pyarrow. Arrow is the boundary, so nothing is copied
into Python objects on the way and nothing is parsed twice.

    import gohep

    tbl = gohep.read_tree("data.root", "tree")     # a pyarrow.Table
    df  = gohep.read_dataframe("data.root", "tree")  # a pandas.DataFrame

What you need for this is the ``root2arrow`` binary on your PATH:

    go install go-hep.org/x/hep/cmd/root2arrow@latest

and no ROOT installation at all, which is the whole idea.
"""

import os
import shutil
import subprocess

__all__ = [
    "read_tree",
    "read_dataframe",
    "read_batches",
    "which",
    "ToolNotFound",
    "ConversionError",
]


class ToolNotFound(Exception):
    """Raised when the go-hep binary this module drives cannot be found."""


class ConversionError(Exception):
    """Raised when the go-hep binary refused to read the file."""


def which(tool="root2arrow"):
    """Return the path to a go-hep binary, or raise ToolNotFound.

    The environment variable GOHEP_ROOT2ARROW overrides the search, for a
    binary that is not on PATH.
    """
    env = os.environ.get("GOHEP_" + tool.upper().replace("-", "_"))
    if env:
        if not os.path.exists(env):
            raise ToolNotFound(f"{env} (from the environment) does not exist")
        return env

    path = shutil.which(tool)
    if path is None:
        raise ToolNotFound(
            f"could not find {tool!r} on PATH.\n"
            f"install it with:\n"
            f"    go install go-hep.org/x/hep/cmd/{tool}@latest"
        )
    return path


def _command(path, tree, tool="root2arrow"):
    """Return the command that converts a tree to an Arrow stream on stdout."""
    return [which(tool), "-t", tree, "-stream", "-o", "-", path]


def read_batches(path, tree="tree"):
    """Yield the record batches of a ROOT tree, as pyarrow.RecordBatch.

    This is the one to use for a tree too large to want in memory at once:
    the batches arrive as go-hep writes them.
    """
    import pyarrow as pa

    cmd = _command(path, tree)
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        reader = pa.ipc.open_stream(proc.stdout)
        for batch in reader:
            yield batch
    finally:
        _, err = proc.communicate()
        if proc.returncode != 0:
            raise ConversionError(
                f"{' '.join(cmd)} failed ({proc.returncode}): "
                f"{err.decode('utf-8', 'replace').strip()}"
            )


def read_tree(path, tree="tree"):
    """Read a ROOT tree and return it as a pyarrow.Table."""
    import pyarrow as pa

    batches = list(read_batches(path, tree))
    if not batches:
        raise ConversionError(f"{path}: tree {tree!r} holds no entries")
    return pa.Table.from_batches(batches)


def read_dataframe(path, tree="tree"):
    """Read a ROOT tree and return it as a pandas.DataFrame."""
    return read_tree(path, tree).to_pandas()
