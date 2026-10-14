package com.mini;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Repository;

// NOTE: legacy code — setter injection back into the service creates a
// constructor/setter circular dependency (kept as an analysis edge case).
@Repository
public class OrderRepository {

    private OrderService orderService;

    @Autowired
    public void setOrderService(OrderService orderService) {
        this.orderService = orderService;
    }

    public String summary(String id) {
        return "row(" + id + ")";
    }
}
