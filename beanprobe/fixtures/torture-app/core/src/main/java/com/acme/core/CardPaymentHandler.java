package com.acme.core;

import org.springframework.stereotype.Component;

@Component
public class CardPaymentHandler implements PaymentHandler {
    @Override
    public void pay(long cents) {
        System.out.println("[card] " + cents);
    }
}
