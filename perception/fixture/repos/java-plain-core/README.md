# core-toolkit (perception fixture)

Plain Java 21, **no Spring** — used to exercise the structure-only path.

With the default `structure_extensions: ["springboot"]` this repo reports
`not-framework` and perception skips it entirely. To analyse it you must run in
structure mode with an empty extension list:

    {"structure_extensions": []}   # in ~/.scv/config.json

Covers: generics, records, abstract classes, interfaces with default methods,
`@FunctionalInterface`, nested and static nested classes, varargs, method
overloading, lambdas, and deliberate mutual recursion (`isEven`/`isOdd`) which
must never be reported as a dependency cycle.
