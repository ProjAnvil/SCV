package com.acme.core.model;

import java.util.List;

public interface Traversable<T> {

    List<T> depthFirst(T start);

    default int size(T start) {
        return depthFirst(start).size();
    }
}
