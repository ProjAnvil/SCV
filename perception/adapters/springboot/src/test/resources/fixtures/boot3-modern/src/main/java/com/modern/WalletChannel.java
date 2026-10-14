package com.modern;

import org.springframework.stereotype.Component;

@Component
public record WalletChannel() implements PaymentChannel {
    @Override
    public String charge(long cents) {
        return "wallet:" + cents;
    }
}
