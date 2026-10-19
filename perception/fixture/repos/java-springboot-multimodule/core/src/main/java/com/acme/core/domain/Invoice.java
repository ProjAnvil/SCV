package com.acme.core.domain;

import java.util.List;

public record Invoice(String id, String accountId, List<Long> amounts) {

    public long total() {
        return amounts.stream().mapToLong(Long::longValue).sum();
    }
}
