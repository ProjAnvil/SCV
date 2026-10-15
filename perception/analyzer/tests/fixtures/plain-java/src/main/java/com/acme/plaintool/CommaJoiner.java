package com.acme.plaintool;

/** String-joiner implementation with a small helper method. */
public class CommaJoiner implements TextJoiner {

    @Override
    public String join(String left, String right) {
        return left + separator() + right;
    }

    String separator() {
        return ",";
    }
}
