package com.acme.order.client;

import org.springframework.stereotype.Component;

@Component
public class FallbackRateClient implements RateClient {

    @Override
    public double rateFor(String customerId) {
        return 0.05d;
    }
}
