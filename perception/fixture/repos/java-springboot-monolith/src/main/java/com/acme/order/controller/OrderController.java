package com.acme.order.controller;

import com.acme.order.domain.Order;
import com.acme.order.domain.OrderStatus;
import com.acme.order.service.OrderService;
import com.acme.order.service.NotificationService;
import jakarta.validation.Valid;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import java.util.List;

/**
 * REST controller with constructor injection. Exercises stereotype
 * RestController plus the controller -> service layer edge.
 */
@RestController
@RequestMapping("/api/orders")
public class OrderController {

    private final OrderService orderService;
    private final NotificationService notificationService;

    public OrderController(OrderService orderService, NotificationService notificationService) {
        this.orderService = orderService;
        this.notificationService = notificationService;
    }

    @GetMapping("/{id}")
    public ResponseEntity<Order> get(@PathVariable String id) {
        return ResponseEntity.ok(orderService.find(id));
    }

    @PostMapping
    public ResponseEntity<Long> price(@Valid @RequestBody Order order) {
        return ResponseEntity.ok(orderService.price(order));
    }

    @GetMapping
    public List<Order> byStatus(@RequestParam OrderStatus status) {
        return List.of();
    }

    @DeleteMapping("/{id}")
    public ResponseEntity<Void> cancel(@PathVariable String id) {
        notificationService.announce(null);
        return ResponseEntity.noContent().build();
    }
}
