package com.acme.order.client;

/** Two implementations, narrowed by @Qualifier in NotificationService. */
public interface Mailer {

    void send(String from, String orderId);
}
