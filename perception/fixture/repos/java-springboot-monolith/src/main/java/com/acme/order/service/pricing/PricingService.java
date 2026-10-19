package com.acme.order.service.pricing;

import com.acme.order.domain.Order;
import com.acme.order.service.OrderService;
import org.springframework.stereotype.Service;

/**
 * Closing edge of the constructor cycle: PricingService -> OrderService.
 * Spring cannot satisfy this, so SCV must grade the cycle as FATAL.
 */
@Service
public class PricingService {

    private final OrderService orderService;
    private final TaxCalculator taxCalculator;

    public PricingService(OrderService orderService, TaxCalculator taxCalculator) {
        this.orderService = orderService;
        this.taxCalculator = taxCalculator;
    }

    public long quote(Order order) {
        long net = order.totalCents();
        return taxCalculator.withTax(net, order.customerId());
    }
}
