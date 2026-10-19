package com.acme.order.service;

import com.acme.order.repository.OrderRepository;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

/**
 * Closing edge of a FIELD-injection cycle: NotificationService -> AuditService
 * -> NotificationService. Spring tolerates this, so SCV must grade it
 * `tolerated`, not `fatal`.
 */
@Service
public class AuditService {

    @Autowired
    private NotificationService notificationService;

    private final OrderRepository repository;

    public AuditService(OrderRepository repository) {
        this.repository = repository;
    }

    public void record(String orderId) {
        repository.findById(orderId).ifPresent(order -> {
            // touch the counterpart so the cycle edge is observable
            notificationService.announce(null);
        });
    }
}
