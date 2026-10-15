package com.example.calls;

// folded T1-review fix: type arguments in extends/implements clauses must be
// erased so the raw simple name resolves in-source (implements Sink<String>
// resolves Sink, not "Sink<String>").
class Box {
}

interface Sink {
}

interface Plain {
}

// interface extends clause with generics -> EXTENDS Sink
interface Chained extends Sink<String> {
}

public class GenericSupers extends Box<Base> implements Sink<String>, Plain {
}

class Base {
}
