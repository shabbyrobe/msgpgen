``msgpgen`` - Alternative code generator wrapper for ``msgp``
=============================================================

``msgpgen`` combines `tinylib/msgp <https://github.com/tinylib/msgp>`_ with
`shabbyrobe/structer <https://github.com/shabbyrobe/structer>`_ to solve a few
issues with the code generating portion of ``tinylib/msgp``.

It supports:

- All the same arguments as the old generator

It differs:

- ``ignore`` and ``shim`` directives must be declared using the full package
  path.

It adds:

- Support for non-local identifiers, by simply generating the code for those
  identifiers in the relevant package.

- Automatic shimming of primitives

- Automatic handling of interface types

- Silencing spurious warnings

- Alternative methods of searching for types, including using a flat file
  containing a whitelist, or searching for all types that implement an
  interface.

It has the following issues (which should all be fixed at some point):

- Import recursion is not limited. ``msgpgen`` will blindly traverse any user
  package it finds in your gopath and will generate code into it.

- Ignored fields (with the tag `msg:"-"`) produce a warning in the output which
  is not currently quashed.

We may be able to support:

- #163 Embedded fields behaviour

It solves the following issues in the msgp tracker:

- #183 Generator output severity labels
- #158 Workaround for types that are defined in another package?
- #141 Directive ignore all
- #128 Best effort warnings re: external types
- #47 Keep track of imports (boy does it ever do that)


Using
-----

To generate msgpack for all types in and under ``mypkg`` that implement
interface ``mypkg.Msg``, then recursively generate all types in other packages
in your GOPATH that those types reference,
use the following::

    //go:generate msgpgen -iface mypkg.Msg -import mypkg/...

.. warning:: The recursion is not currenty limited by the -import flag, though
   it will be.

