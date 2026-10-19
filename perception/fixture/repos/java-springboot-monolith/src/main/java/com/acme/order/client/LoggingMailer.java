package com.acme.order.client;

import org.springframework.stereotype.Component;

@Component
public class LoggingMailer implements Mailer {

    @Override
    public void send(String from, String orderId) {
        // no-op fixture
    }
}
