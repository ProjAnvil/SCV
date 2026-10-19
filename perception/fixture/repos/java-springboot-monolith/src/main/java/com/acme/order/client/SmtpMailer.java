package com.acme.order.client;

import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.stereotype.Component;

@Qualifier("smtpMailer")
@Component
public class SmtpMailer implements Mailer {

    @Override
    public void send(String from, String orderId) {
        // no-op fixture
    }
}
