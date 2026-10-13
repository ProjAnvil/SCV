package com.acme;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;

@Component
public class CircularB {

    @Autowired
    private CircularA other; // cycle: B -> A -> B

    public String pong() {
        return "b" + other.tag();
    }

    String tag() {
        return "B";
    }
}
