package com.acme.bridge;

import com.acme.bridge.Greetable;

public class Greeter implements Greetable {
    @Override
    public String greet() {
        return "hello";
    }
}
