package com.example.demo;

import org.springframework.stereotype.Component;

@Component("smsSender")
public class SmsNotificationSender implements NotificationSender {
    @Override
    public void send(String message) {
        System.out.println("[sms] " + message);
    }
}
