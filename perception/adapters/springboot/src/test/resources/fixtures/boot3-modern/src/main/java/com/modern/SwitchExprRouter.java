package com.modern;

import org.springframework.stereotype.Component;

// Java 21 syntax in the body: switch expressions, var, text blocks
@Component
public class SwitchExprRouter {

    private final CheckoutService checkout;

    public SwitchExprRouter(CheckoutService checkout) {
        this.checkout = checkout;
    }

    public String route(String kind, long cents) {
        var prefix = switch (kind) {
            case "card" -> "C";
            case "wallet" -> "W";
            default -> "?";
        };
        var json = """
                {"kind":"%s"}""".formatted(kind);
        return prefix + json + ":" + checkout.checkout(cents);
    }
}
