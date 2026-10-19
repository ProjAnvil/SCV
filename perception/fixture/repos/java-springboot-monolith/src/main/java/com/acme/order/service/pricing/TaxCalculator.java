package com.acme.order.service.pricing;

import com.acme.order.client.RateClient;
import org.springframework.stereotype.Component;

@Component
public class TaxCalculator {

    private final RateClient rateClient;

    public TaxCalculator(RateClient rateClient) {
        this.rateClient = rateClient;
    }

    public long withTax(long netCents, String customerId) {
        double rate = rateClient.rateFor(customerId);
        return Math.round(netCents * (1.0 + rate));
    }
}
