``msgpgen`` - Alternative code generator wrapper for ``msgp``
=============================================================

``msgpgen`` combines `tinylib/msgp <https://github.com/tinylib/msgp>`_ with
`shabbyrobe/structer <https://github.com/shabbyrobe/structer>`_ to solve a few
issues with the code generating portion of ``tinylib/msgp``.

It supports:

- All the same arguments as the old generator

It adds:

- Support for non-local identifiers, by simply generating the code for those
  identifiers in the relevant package.

- Automatic shimming of primitives

- Silencing spurious warnings

- Alternative methods of searching for types, including using a flat file
  containing a whitelist, or searching for all types that implement an
  interface.

It does not yet support:

- Limiting the import recursion. ``msgpgen`` will blindly traverse any user
  package it finds in your gopath and will generate code into it.


Using
-----

To generate msgpack for all types in and under ``mypkg`` that implement
interface ``mypkg.Msg``, then recursively generate all types in other packages
in your GOPATH that those types reference,
use the following::

    //go:generate msgpgen -iface mypkg.Msg -import mypkg/...

.. warning:: The recursion is not currenty limited by the -import flag, though
   it will be.


To generate msgpack for all types listed in ``myfile.tsv``, then recursively
generate all types in other packages in your GOPATH that those types reference,
use the following::

    //go:generate msgpgen -mode tsv -file mytypes.tsv

Entries in the tsv file must be in the format ``full/path/to/package.Type``.
TSV files are split by spaces or tabs, like awk, and the column can be specified
using the 1-indexed ``-col`` argument.

