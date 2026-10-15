package com.sb.beans;

import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.stereotype.Component;

import java.time.Clock;

// §5: @Bean methods (incl. STATIC), @Bean params as injection points, and
// @Bean-produced suppression — GreetingClient's own constructor params are
// NOT class-level injection points.
@Configuration
class BeanConfig {

    @Bean
    public static Clock clock() {
        return Clock.systemUTC();
    }

    @Bean
    public GreetingClient greetingClient(@Qualifier("emailSender") Sender sender, Clock clock) {
        return new GreetingClient(sender, clock);
    }
}

interface Sender {
    void send(String message);
}

@Component("emailSender")
class EmailSender implements Sender {
    public void send(String message) {
    }
}

@Component
class SmsSender implements Sender {
    public void send(String message) {
    }
}

class GreetingClient {

    GreetingClient(Sender sender, Clock clock) {
    }
}
