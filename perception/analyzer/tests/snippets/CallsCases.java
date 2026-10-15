package com.example.calls;

import static java.util.Objects.requireNonNull;

// CALLS/REFERENCES matrix (spec §4): receiver resolution over fields, params,
// locals, implicit/explicit this, class-name statics; dispatch kind from the
// receiver's declared type; overload ambiguity and static-import drops.
public class CallsCases {

    private final Worker worker;   // field receiver, in-source class -> direct
    private final Job job;         // field receiver, in-source interface -> virtual-dispatch

    public CallsCases(Worker worker, Job job) {
        this.worker = worker;
        this.job = job;
    }

    public void directViaField() {
        worker.run();
    }

    public void virtualViaField() {
        job.run();
    }

    public void viaParamAndLocal(Tool param) {
        param.use();
        Tool local = new Tool();
        local.use();
    }

    public void viaThis() {
        own();
        this.own();
        this.worker.run();
    }

    public void viaClassName() {
        CallsCases.own();
    }

    void own() {
    }

    public void uniqueOverload(Tool t) {
        t.go(1);
    }

    public void ambiguousOverload(Tool t) {
        t.go("x", 1);
    }

    public void staticImportUse() {
        requireNonNull(worker);
        Objects.requireNonNull(job);
    }

    public void unresolvedReceivers() {
        String s = "x";
        s.trim();
    }

    public void noSuchMethod() {
        mystery();
    }

    public void references() {
        Tool t = new Tool();
        Plain p = new Plain();
        String s = new String("no");
        Object o = null;
        Plain q = (Plain) o;
        String r = (String) o;
    }
}
