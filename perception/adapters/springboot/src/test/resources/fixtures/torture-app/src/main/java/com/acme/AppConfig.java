package com.acme;

import com.acme.core.FeatureProperties;
import com.acme.core.GreetingSender;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.time.Clock;

@Configuration
@EnableConfigurationProperties(FeatureProperties.class)
public class AppConfig {

    // torture: STATIC @Bean method
    @Bean
    public static Clock clock() {
        return Clock.systemUTC();
    }

    // torture: @Bean with @Qualifier param, returns a plain (non-annotated) class
    @Bean
    public GreetingClient greetingClient(@Qualifier("emailGreetingSender") GreetingSender sender, Clock clock) {
        return new GreetingClient(sender, clock);
    }

    // torture: default/override pattern — runtime has exactly ONE SchedulingPolicy,
    // statically there are two candidates -> honest ambiguity expected
    @Bean
    @ConditionalOnMissingBean
    public SchedulingPolicy defaultPolicy() {
        return () -> 10;
    }

    @Bean
    @ConditionalOnProperty(name = "sched.custom.enabled", havingValue = "true")
    public SchedulingPolicy customPolicy() {
        return () -> 1;
    }
}
