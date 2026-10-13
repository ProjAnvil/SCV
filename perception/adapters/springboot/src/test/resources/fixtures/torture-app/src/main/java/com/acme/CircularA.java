package com.acme;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;

@Component
public class CircularA {

    @Autowired
    private CircularB other; // cycle: A -> B -> A

    public String ping() {
        return "a" + other.tag();
    }

    String tag() {
        return "A";
    }
}
