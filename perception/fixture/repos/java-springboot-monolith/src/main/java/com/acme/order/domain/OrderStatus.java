package com.acme.order.domain;

public enum OrderStatus {
    DRAFT,
    PENDING,
    SETTLED,
    REFUNDED,
    CANCELLED;

    public boolean terminal() {
        return this == SETTLED || this == REFUNDED || this == CANCELLED;
    }
}
