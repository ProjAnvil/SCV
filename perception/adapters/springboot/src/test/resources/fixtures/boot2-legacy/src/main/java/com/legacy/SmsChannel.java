package com.legacy;

import org.springframework.stereotype.Component;

@Component
public class SmsChannel implements NotificationChannel {
    @Override
    public String send(String message) {
        return "sms:" + message;
    }
}
