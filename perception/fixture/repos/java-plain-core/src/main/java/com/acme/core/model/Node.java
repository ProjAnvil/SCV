package com.acme.core.model;

/** Generic record: exercises generics + records in the structure layer. */
public record Node<T extends Comparable<T>>(T value, Node<T> next) {

    public boolean greaterThan(Node<T> other) {
        return value.compareTo(other.value()) > 0;
    }
}
