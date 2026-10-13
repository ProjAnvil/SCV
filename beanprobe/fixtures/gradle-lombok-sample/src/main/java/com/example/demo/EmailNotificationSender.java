package com.example.demo;

import org.springframework.stereotype.Component;

@Component("emailSender")
public class EmailNotificationSender implements NotificationSender {
    @Override
    public void send(String message) {
        System.out.println("[email] " + message);
    }
}
