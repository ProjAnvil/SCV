package com.example.calls;

// overload matrix: go(int) is unique at arity 1; the two arity-2 overloads
// make any arity-2 call ambiguous under the syntactic name+arity rule.
public class Tool {

    public void use() {
    }

    public void go(int n) {
    }

    public void go(String a, int b) {
    }

    public void go(int a, String b) {
    }
}
