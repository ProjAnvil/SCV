package com.example.demo;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

@Service
public class ReportService {

    @Autowired
    private NotificationSender notificationSender; // no qualifier: ambiguous (email vs sms)

    private GreetingRepository greetingRepository;

    @Autowired
    public void setGreetingRepository(GreetingRepository greetingRepository) {
        this.greetingRepository = greetingRepository;
    }

    public String buildReport() {
        notificationSender.send("report ready");
        return greetingRepository.loadTemplate() + " (report)";
    }
}
