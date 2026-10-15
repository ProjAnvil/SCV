package com.example.methods;

import java.util.List;
import java.util.Map;
import java.util.Objects;
import static java.util.Objects.requireNonNull;

// method-key normalization matrix: generics erased, arrays/varargs -> [],
// ctor named after the class, one overload pair colliding after erasure.
public class Methods {

    public Methods(String name, List<String> tags, String... rest) {
    }

    public <T> T pick(Map<String, List<Integer>> m) {
        return null;
    }

    void handle(int[][] grid, boolean flag) {
    }

    private static String save(List<String> a) {
        return "";
    }

    // normalizes to the same key as save(List<String>) -> key collision
    public int save(List<Integer> b) {
        return 0;
    }
}
