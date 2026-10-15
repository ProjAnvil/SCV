package com.example.kinds;

// five declaration kinds in one file: class, interface, record, enum,
// annotation. The record deliberately has NO canonical constructor written.
public class Kinds {
}

interface Shape {
    double area();
}

record Point(int x, int y) {
}

enum Color {
    RED, GREEN;

    // enum members live under enum_body_declarations, one level below
    // enum_body's direct children — they must still be declared methods
    void rotate() {
    }
}

@interface Marker {
    String value();
}
