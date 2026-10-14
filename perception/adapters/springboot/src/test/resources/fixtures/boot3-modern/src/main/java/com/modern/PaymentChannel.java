package com.modern;

// Java 21: sealed service contract with record implementations
public sealed interface PaymentChannel permits CardChannel, WalletChannel {
    String charge(long cents);
}
