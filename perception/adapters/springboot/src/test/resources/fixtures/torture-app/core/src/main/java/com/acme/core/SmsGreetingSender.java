package com.acme.core;

import org.springframework.context.annotation.Primary;
import org.springframework.stereotype.Component;

// torture: @Primary — unqualified injection of GreetingSender should resolve here
@Component
@Primary
public class SmsGreetingSender implements GreetingSender {
    @Override
    public void send(String message) {
        System.out.println("[sms] " + message);
    }
}
