package com.acme.order.domain;

import java.util.Objects;

public final class OrderLine {

    private final String sku;
    private final int quantity;
    private final long unitPriceCents;

    public OrderLine(String sku, int quantity, long unitPriceCents) {
        this.sku = Objects.requireNonNull(sku, "sku");
        this.quantity = quantity;
        this.unitPriceCents = unitPriceCents;
    }

    public String sku() {
        return sku;
    }

    public int quantity() {
        return quantity;
    }

    public long unitPriceCents() {
        return unitPriceCents;
    }

    public long subtotalCents() {
        return quantity * unitPriceCents;
    }
}
