package com.acme.core.service;

import java.util.concurrent.ExecutorService;

/** Concrete subclass: exercises the implements/extends edges. */
public class AsyncDispatcher<T> extends AbstractDispatcher<T> {

    private final ExecutorService executor;

    public AsyncDispatcher(ExecutorService executor) {
        this.executor = executor;
    }

    @Override
    protected void doDispatch(T payload) {
        executor.submit(() -> handle(payload));
    }

    private void handle(T payload) {
        // no-op fixture
    }
}
