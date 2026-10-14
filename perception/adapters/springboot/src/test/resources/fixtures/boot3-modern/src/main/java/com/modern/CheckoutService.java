package com.modern;

import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;

// unqualified PaymentChannel injection resolves to the @Primary CardChannel
@Service
@RequiredArgsConstructor
public class CheckoutService {

    private final PaymentChannel channel;
    private final AuditSink audit;

    public String checkout(long cents) {
        String result = channel.charge(cents);
        audit.record(result);
        return result;
    }
}
