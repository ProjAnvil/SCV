package com.b2m.service;

import com.b2m.domain.CustomerRepository;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

@Service
public class LoyaltyService {

    @Autowired
    private CustomerRepository repository;

    public long points(long id) {
        return repository.find(id).length();
    }
}
