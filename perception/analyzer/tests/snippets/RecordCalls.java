package com.example.calls;

// record components are the record's implicit fields: receivers resolve.
public record RecordCalls(Engine engine) {

    public void drive() {
        engine.start();
    }
}

interface Engine {

    void start();
}
