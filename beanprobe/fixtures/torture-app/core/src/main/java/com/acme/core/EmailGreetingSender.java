package com.acme.core;

import org.springframework.stereotype.Component;

@Component("emailGreetingSender")
public class EmailGreetingSender implements GreetingSender {
    @Override
    public void send(String message) {
        System.out.println("[email] " + message);
    }
}
