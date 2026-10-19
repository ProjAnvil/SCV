package com.acme.core.service;

public interface Dispatcher<T> {

    void dispatch(T payload);
}
