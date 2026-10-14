package com.legacy;

import org.springframework.web.bind.annotation.ControllerAdvice;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.ResponseBody;

@ControllerAdvice
public class GlobalAdvice {

    private final FancyRepository repository;

    public GlobalAdvice(FancyRepository repository) {
        this.repository = repository;
    }

    @ExceptionHandler(IllegalArgumentException.class)
    @ResponseBody
    public String handle(IllegalArgumentException e) {
        return repository.name();
    }
}
