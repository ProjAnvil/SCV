package com.acme.core.service;

import java.util.ArrayList;
import java.util.List;

/** Abstract base with a template method and an abstract hook. */
public abstract class AbstractDispatcher<T> implements Dispatcher<T> {

    private final List<Listener<T>> listeners = new ArrayList<>();

    @Override
    public final void dispatch(T payload) {
        before(payload);
        doDispatch(payload);
        listeners.forEach(listener -> listener.onEvent(payload));
    }

    public void register(Listener<T> listener) {
        listeners.add(listener);
    }

    protected void before(T payload) {
        // default no-op
    }

    protected abstract void doDispatch(T payload);
}
