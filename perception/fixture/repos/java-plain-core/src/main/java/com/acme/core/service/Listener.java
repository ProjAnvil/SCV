package com.acme.core.service;

@FunctionalInterface
public interface Listener<T> {

    void onEvent(T payload);
}
