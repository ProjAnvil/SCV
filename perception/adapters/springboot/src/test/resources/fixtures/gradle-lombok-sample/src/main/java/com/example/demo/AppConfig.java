package com.example.demo;

import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.time.Clock;

@Configuration
public class AppConfig {

    @Bean
    public Clock systemClock() {
        return Clock.systemUTC();
    }

    @Bean
    public GreetingClient greetingClient(GreetingService greetingService, Clock clock) {
        return new GreetingClient(greetingService, clock);
    }
}
