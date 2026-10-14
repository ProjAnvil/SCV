package com.legacy;

import org.springframework.stereotype.Component;

@Component
public class EmailChannel implements NotificationChannel {
    @Override
    public String send(String message) {
        return "email:" + message;
    }
}
