package com.acme.plaintool;

/** Entry point — dispatches through the interface receiver. */
public final class App {

    public static void main(String[] args) {
        TextJoiner joiner = new CommaJoiner();
        System.out.println(joiner.join("hello", "world"));
    }
}
