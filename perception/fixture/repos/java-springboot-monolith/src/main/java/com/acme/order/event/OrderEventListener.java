package com.acme.order.event;

import com.acme.order.domain.Order;
import com.acme.order.service.OrderPlacedListener;
import com.acme.order.service.OrderService;
import org.springframework.stereotype.Component;

/** Implements the listener interface: exercises interface-implementation edges. */
@Component
public class OrderEventListener implements OrderPlacedListener {

    private final OrderService orderService;

    public OrderEventListener(OrderService orderService) {
        this.orderService = orderService;
    }

    @Override
    public void onPlaced(OrderPlaced event) {
        Order order = orderService.find(event.orderId());
        orderService.price(order);
    }
}
