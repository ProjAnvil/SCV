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
    RED, GREEN
}

@interface Marker {
    String value();
}
