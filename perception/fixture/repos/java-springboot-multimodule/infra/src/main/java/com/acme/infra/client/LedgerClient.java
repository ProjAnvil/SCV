package com.acme.infra.client;

import org.springframework.stereotype.Component;

/** Third-tier component: no outgoing injection. */
@Component
public class LedgerClient {

    public void post(String accountId, long cents) {
        // no-op fixture
    }
}
