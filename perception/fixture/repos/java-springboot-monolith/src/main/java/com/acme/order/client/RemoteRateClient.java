package com.acme.order.client;

import org.springframework.context.annotation.Primary;
import org.springframework.stereotype.Component;

@Primary
@Component
public class RemoteRateClient implements RateClient {

    @Override
    public double rateFor(String customerId) {
        return 0.07d;
    }
}
