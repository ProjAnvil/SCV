package com.acme.algo;

/**
 * Mutual recursion on purpose: a CALLS cycle in the structure layer. SCV must
 * not report this as a dependency cycle — CALLS recursion is not a user-visible
 * container dependency.
 */
public final class MutualRecursion {

    private MutualRecursion() {
    }

    public static boolean isEven(int n) {
        return n == 0 || isOdd(n - 1);
    }

    public static boolean isOdd(int n) {
        return n != 0 && isEven(n - 1);
    }
}
