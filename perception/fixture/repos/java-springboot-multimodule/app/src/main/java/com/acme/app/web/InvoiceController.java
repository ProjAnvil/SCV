package com.acme.app.web;

import com.acme.core.domain.Invoice;
import com.acme.core.service.InvoiceService;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/** Cross-module injection: app depends on core. */
@RestController
@RequestMapping("/api/invoices")
public class InvoiceController {

    private final InvoiceService invoiceService;

    public InvoiceController(InvoiceService invoiceService) {
        this.invoiceService = invoiceService;
    }

    @PostMapping
    public void settle(Invoice invoice) {
        invoiceService.settle(invoice);
    }

    @GetMapping
    public String ping() {
        return "ok";
    }
}
