package com.modern;

import org.springframework.context.annotation.Primary;
import org.springframework.stereotype.Component;

// record bean with constructor binding of a properties record; @Primary wins
// unqualified PaymentChannel injection
@Primary
@Component
public record CardChannel(PaymentProps props) implements PaymentChannel {
    @Override
    public String charge(long cents) {
        return "card:" + props.label() + ":" + cents;
    }
}
