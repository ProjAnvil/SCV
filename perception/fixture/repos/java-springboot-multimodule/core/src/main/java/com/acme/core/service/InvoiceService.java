package com.acme.core.service;

import com.acme.core.domain.Invoice;
import com.acme.infra.client.LedgerClient;
import org.springframework.stereotype.Service;

/** Cross-module injection: core depends on infra. */
@Service
public class InvoiceService {

    private final LedgerClient ledgerClient;

    public InvoiceService(LedgerClient ledgerClient) {
        this.ledgerClient = ledgerClient;
    }

    public void settle(Invoice invoice) {
        ledgerClient.post(invoice.accountId(), invoice.total());
    }
}
