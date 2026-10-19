package com.acme.order.controller;

import com.acme.order.service.OrderService;
import org.springframework.web.bind.annotation.*;

/**
 * Deliberately uses an on-demand (wildcard) import. Known gap: the analyzer
 * cannot resolve {@code @RestController} to its FQN through a wildcard import,
 * so this class is extracted in the structure layer but is NOT registered as a
 * bean. Kept in the fixture so the gap is frozen and visible.
 */
@RestController
@RequestMapping("/api/status")
public class StatusController {

    private final OrderService orderService;

    public StatusController(OrderService orderService) {
        this.orderService = orderService;
    }

    @GetMapping
    public String ping() {
        return "ok";
    }
}
