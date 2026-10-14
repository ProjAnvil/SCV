package com.mini;

import org.springframework.stereotype.Service;

@Service
public class OrderService {

    private final OrderRepository repository;

    public OrderService(OrderRepository repository) {
        this.repository = repository;
    }

    public String describe(String id) {
        return "order " + id + ": " + repository.summary(id);
    }
}
