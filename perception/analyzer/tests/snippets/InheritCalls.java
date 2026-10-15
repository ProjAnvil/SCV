package com.example.calls;

// fields inherited from an in-source superclass are in scope (owner chain).
class BaseHolder {

    protected final Worker inherited;

    BaseHolder(Worker inherited) {
        this.inherited = inherited;
    }
}

public class InheritCalls extends BaseHolder {

    public InheritCalls(Worker inherited) {
        super(inherited);
    }

    public void act() {
        inherited.run();
    }
}
