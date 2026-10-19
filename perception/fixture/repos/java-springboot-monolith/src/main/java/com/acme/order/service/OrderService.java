package com.acme.order.service;

import com.acme.order.domain.Order;
import com.acme.order.repository.OrderRepository;
import com.acme.order.service.pricing.PricingService;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/**
 * Constructor injection into a cycle on purpose: OrderService -> PricingService
 * -> OrderService via constructor, which SCV must grade as a FATAL cycle.
 */
@Service
public class OrderService {

    private final OrderRepository repository;
    private final PricingService pricing;

    public OrderService(OrderRepository repository, PricingService pricing) {
        this.repository = repository;
        this.pricing = pricing;
    }

    @Transactional(readOnly = true)
    public Order find(String id) {
        return repository.findById(id).orElseThrow();
    }

    public long price(Order order) {
        return pricing.quote(order);
    }
}
