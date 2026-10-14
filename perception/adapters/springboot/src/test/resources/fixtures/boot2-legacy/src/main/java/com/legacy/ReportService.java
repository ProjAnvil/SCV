package com.legacy;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

// legacy: constructor mixing @Value with real beans + field injection of the
// competing profile GreetingService beans
@Service
public class ReportService {

    private final FancyRepository repository;

    @Autowired
    private GreetingService greetingService; // mock + real compete -> ambiguous

    public ReportService(@Value("${report.name:rep}") String name, FancyRepository repository) {
        this.repository = repository;
    }
}
