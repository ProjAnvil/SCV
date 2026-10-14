package com.legacy;

import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Profile;

import java.time.Clock;

@Configuration
public class AppConfig {

    @Bean
    public Clock clock() {
        return Clock.systemUTC();
    }

    // competing profile beans: runtime has exactly one GreetingService
    @Bean
    @Profile("dev")
    public GreetingService mockGreetingService() {
        return () -> "mock";
    }

    @Bean
    @Profile("prod")
    public GreetingService realGreetingService(FancyRepository repository) {
        return () -> "real:" + repository.name();
    }
}
