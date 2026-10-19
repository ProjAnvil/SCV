package com.acme.order.client;

/** Interface with two implementations, resolved by @Primary and @Qualifier. */
public interface RateClient {

    double rateFor(String customerId);
}
