package com.acme.core.model;

import java.util.*;

/** Generic container with a nested static builder and a static nested class. */
public final class Graph<T extends Comparable<T>> implements Traversable<T> {

    private final Map<T, List<T>> adjacency = new HashMap<>();

    public static final class Builder<T extends Comparable<T>> {
        private final Graph<T> graph = new Graph<>();

        public Builder<T> edge(T from, T to) {
            graph.adjacency.computeIfAbsent(from, k -> new ArrayList<>()).add(to);
            return this;
        }

        public Graph<T> build() {
            return graph;
        }
    }

    public void connect(T from, T to) {
        adjacency.computeIfAbsent(from, k -> new ArrayList<>()).add(to);
    }

    public List<T> neighbours(T of) {
        return adjacency.getOrDefault(of, List.of());
    }

    @Override
    public List<T> depthFirst(T start) {
        List<T> out = new ArrayList<>();
        Set<T> seen = new HashSet<>();
        visit(start, out, seen);
        return out;
    }

    private void visit(T node, List<T> out, Set<T> seen) {
        if (!seen.add(node)) {
            return;
        }
        out.add(node);
        for (T next : neighbours(node)) {
            visit(next, out, seen);   // self-recursion
        }
    }
}
