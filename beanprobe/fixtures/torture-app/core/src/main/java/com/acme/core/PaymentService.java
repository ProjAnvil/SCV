package com.acme.core;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

// torture: by-name disambiguation — two PaymentHandler impls, no @Primary.
// Spring falls back to matching the FIELD NAME against bean names.
@Service
public class PaymentService {

    @Autowired
    private PaymentHandler cardPaymentHandler; // matches bean "cardPaymentHandler" by name

    @Autowired
    private PaymentHandler fallbackHandler;    // matches nothing -> genuinely ambiguous

    public void checkout(long cents) {
        cardPaymentHandler.pay(cents);
        fallbackHandler.pay(cents);
    }
}
