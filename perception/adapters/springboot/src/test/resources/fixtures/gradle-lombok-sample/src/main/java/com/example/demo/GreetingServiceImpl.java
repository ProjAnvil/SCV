package com.example.demo;

import lombok.RequiredArgsConstructor;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.stereotype.Service;

@Service
@RequiredArgsConstructor
public class GreetingServiceImpl implements GreetingService {

    private final GreetingRepository greetingRepository;

    @Qualifier("emailSender")
    private final NotificationSender notificationSender;

    @Override
    public String greet(String name) {
        String template = greetingRepository.loadTemplate();
        notificationSender.send("greeting prepared for " + name);
        return template.replace("%s", name);
    }
}
