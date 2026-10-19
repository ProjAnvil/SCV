package com.acme.order.service;

import com.acme.order.client.Mailer;
import com.acme.order.event.OrderPlaced;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Lazy;
import org.springframework.context.annotation.Profile;
import org.springframework.stereotype.Service;

import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.function.Function;

/**
 * Mixed injection styles on purpose:
 * - field @Autowired (tolerated cycle with AuditService)
 * - @Qualifier narrowing two Mailer implementations
 * - collection injection (List, Map)
 * - Optional and @Lazy dependency
 * - @Value on a constructor parameter
 * - setter injection for the optional listener
 */
@Service
@Profile("!test")
public class NotificationService {

    @Autowired
    private AuditService auditService;

    private final Mailer primaryMailer;
    private final List<Mailer> allMailers;
    private final Map<String, Mailer> mailersByName;
    private final Optional<Mailer> maybeMailer;
    private final Function<String, Boolean> gateway;
    private final String sender;

    private OrderPlacedListener listener;

    public NotificationService(
            @Qualifier("smtpMailer") Mailer primaryMailer,
            List<Mailer> allMailers,
            Map<String, Mailer> mailersByName,
            Optional<Mailer> maybeMailer,
            @Lazy Function<String, Boolean> gateway,
            @Value("${acme.mail.sender:noreply@acme.test}") String sender) {
        this.primaryMailer = primaryMailer;
        this.allMailers = allMailers;
        this.mailersByName = mailersByName;
        this.maybeMailer = maybeMailer;
        this.gateway = gateway;
        this.sender = sender;
    }

    @Autowired
    public void setListener(OrderPlacedListener listener) {
        this.listener = listener;
    }

    public void announce(OrderPlaced event) {
        if (!gateway.apply(event.orderId())) {
            return;
        }
        primaryMailer.send(sender, event.orderId());
        allMailers.forEach(mailer -> mailer.send(sender, event.orderId()));
        maybeMailer.ifPresent(mailer -> mailer.send(sender, event.orderId()));
        auditService.record(event.orderId());
        if (listener != null) {
            listener.onPlaced(event);
        }
    }
}
